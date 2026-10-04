import React from 'react'
import {
  Create,
  SimpleForm,
  TextInput,
  BooleanInput,
  required,
  useTranslate,
  useRefresh,
  useNotify,
  useRedirect,
} from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import { Title } from '../common'
import { pinkPageStyles, PinkPageTheme } from '../common/pinkPageStyles'

const useStyles = makeStyles((theme) => ({
  ...pinkPageStyles(theme),
  form: {
    width: '100%',
    maxWidth: 760,
    margin: '0 auto',
    boxSizing: 'border-box',
  },
}))

const PlaylistCreate = (props) => {
  const { basePath } = props
  const refresh = useRefresh()
  const notify = useNotify()
  const redirect = useRedirect()
  const translate = useTranslate()
  const classes = useStyles()
  const resourceName = translate('resources.playlist.name', { smart_count: 1 })
  const title = translate('ra.page.create', {
    name: `${resourceName}`,
  })

  const onSuccess = () => {
    notify('ra.notification.created', 'info', { smart_count: 1 })
    redirect('list', basePath)
    refresh()
  }

  return (
    <PinkPageTheme>
      <div className={classes.root}>
        <Create
          title={<Title subTitle={title} />}
          {...props}
          onSuccess={onSuccess}
        >
          <SimpleForm
            className={classes.form}
            redirect="list"
            variant={'outlined'}
          >
            <TextInput source="name" validate={required()} />
            <TextInput multiline source="comment" />
            <BooleanInput source="public" initialValue={true} />
          </SimpleForm>
        </Create>
      </div>
    </PinkPageTheme>
  )
}

export default PlaylistCreate
